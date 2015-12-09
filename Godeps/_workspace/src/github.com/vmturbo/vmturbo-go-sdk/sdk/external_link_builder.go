package sdk

// import (
// 	"fmt"
// )

type ExternalEntityLinkBuilder struct {
	entityLink *ExternalEntityLink
}

func NewExternalEntityLinkBuilder() *ExternalEntityLinkBuilder {
	link := &ExternalEntityLink{}
	return &ExternalEntityLinkBuilder{
		entityLink: link,
	}
}

// Initialize the buyer/seller external link that you're building.
// This method sets the entity types for the buyer and seller, as well as the type of provider
// relationship HOSTING or code LAYEREDOVER.
func (this *ExternalEntityLinkBuilder) Link(buyer, seller EntityDTO_EntityType, relationship Provider_ProviderType) *ExternalEntityLinkBuilder {
	this.entityLink.BuyerRef = &buyer
	this.entityLink.SellerRef = &seller
	this.entityLink.Relationship = &relationship

	return this
}

// Add a single bought commodity to the link.
func (this *ExternalEntityLinkBuilder) Commodity(comm CommodityDTO_CommodityType) *ExternalEntityLinkBuilder {
	commodities := this.entityLink.GetCommodities()
	commodities = append(commodities, comm)

	this.entityLink.Commodities = commodities
	return this
}

/**
     * Set a property of the discovered entity to the link. Operations Manager will use this property to
     * stitch the discovered entity into the Operations Manager topology. This setting includes the property name
     * and an arbitrary description.
     * <p>
     * The property name specifies which property of the discovered entity you want to match with this link. The discovered
     * entity's DTO contains the list of properties for that entity. The link you're building must include a property that matches a
     * named property in that DTO. Note that the SDK includes builders for different types of entities.
     * These builders add properties to the entity DTO, giving them names from the {@link SupplyChainConstants} enumeration.
     * The properties you create here match the property names in the target DTO.
	 * For example, the {link ApplicationBuilder} adds an IP address as a property named {@code SupplyChainConstants.IP_ADDRESS}.
	 * To match the application IP address in this link, add a property to the link with the same name. By doing that,
	 * the stitching process can access the value that is set in the discovered entity's DTO.
	 * <p>
	 * The property description is an arbitrary string to describe the purpose of this property. This is useful
	 * when you print out the link via a {@code toString()} method.
     *
     * @param name         Entity property name
     * @param description  Property description
     *
     * @return             {@link ExternalLinkBuilder}
*/
func (this *ExternalEntityLinkBuilder) ProbeEntityPropertyDef(name, description string) *ExternalEntityLinkBuilder {
	entityProperty := &ExternalEntityLink_EntityPropertyDef{
		Name:        &name,
		Description: &description,
	}
	currentProps := this.entityLink.GetProbeEntityPropertyDef()
	currentProps = append(currentProps, entityProperty)
	this.entityLink.ProbeEntityPropertyDef = currentProps

	return this
}

/**
 * Set an {@link ServerEntityPropertyDef} to the link you're building.
 * The {@code ServerEntityPropertyDef} includes metadata for the properties of the
 * external entity. Operations Manager can use the metadata
 * to stitch entities discovered by the probe together with external entities.
 * An external entity is one that exists in the Operations Manager topology, but has
 * not been discovered by the probe.
 *
 * @param propertyDef   An {@code ExternalEntityLinkDef} instance
 * @return              {@link ExternalLinkBuilder}
 */
func (this *ExternalEntityLinkBuilder) ExternalEntityPropertyDef(propertyDef *ExternalEntityLink_ServerEntityPropDef) *ExternalEntityLinkBuilder {
	currentExtProps := this.entityLink.GetExternalEntityPropertyDefs()
	currentExtProps = append(currentExtProps, propertyDef)

	this.entityLink.ExternalEntityPropertyDefs = currentExtProps

	return this
}

/**
 * Get the {@code ExternalEntityLink} that you have built.
 *
 * @return  {@link ExternalEntityLink}
 */
func (this *ExternalEntityLinkBuilder) Build() *ExternalEntityLink {
	return this.entityLink
}
